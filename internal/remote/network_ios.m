//go:build ios && cgo

#import <Foundation/Foundation.h>

extern void gorexNetworkResult(long long token, char *message);
static NSMutableDictionary<NSNumber *, NSURLSessionDataTask *> *networkTasks;

void gorex_network_start(long long token, const char *rawURL) {
 NSString *urlString = [NSString stringWithUTF8String:rawURL];
 dispatch_async(dispatch_get_main_queue(), ^{
  if (!networkTasks) networkTasks = [NSMutableDictionary new];
  NSMutableURLRequest *request = [NSMutableURLRequest requestWithURL:[NSURL URLWithString:urlString]];
  request.HTTPMethod = @"HEAD";
  request.timeoutInterval = 10;
  NSURLSessionDataTask *task = [NSURLSession.sharedSession dataTaskWithRequest:request completionHandler:^(NSData *data, NSURLResponse *response, NSError *error) {
   dispatch_async(dispatch_get_main_queue(), ^{
    [networkTasks removeObjectForKey:@(token)];
    gorexNetworkResult(token, error ? (char *)error.localizedDescription.UTF8String : NULL);
   });
  }];
  networkTasks[@(token)] = task;
  [task resume];
 });
}

void gorex_network_cancel(long long token) {
 dispatch_async(dispatch_get_main_queue(), ^{
  [networkTasks[@(token)] cancel];
  [networkTasks removeObjectForKey:@(token)];
 });
}
